package main

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"net"
	"regexp"
	"strings"
	"time"
)

/*********************************************************** class ChatMember *****************************************************************/

type ChatMember struct {
	msgChannel     *MessageChannel
	pseudo         string
	msgChannelPort chan Message
	inputsPort     chan string
	quitPort       chan struct{}
	conn           net.Conn
	server         *Server
}

func NewChatMember(pseudo string, conn net.Conn, server *Server, idleness int) *ChatMember {
	chatMember := &ChatMember{
		pseudo:         pseudo,
		msgChannelPort: make(chan Message, 5),
		inputsPort:     make(chan string),
		quitPort:       make(chan struct{}),
		conn:           conn,
		server:         server,
	}
	server.AddMember(chatMember)
	chatMember.startListening(idleness)
	return chatMember
}

func (chatMember *ChatMember) JoinChannel(msgChannel *MessageChannel) {
	chatMember.msgChannel = msgChannel
	msgChannel.SubscribeAsync(chatMember)
}

func (chatMember *ChatMember) QuitChannel(reason UnSubscribReason) {
	chatMember.msgChannel.UnSubscribeAsync(chatMember, reason)
	chatMember.msgChannel = nil
}

func (chatMember *ChatMember) ChangeChannel(channel *MessageChannel) {
	if chatMember.IsConnectedToAChannel() {
		chatMember.QuitChannel(QUIT_CHANNEL)
	}
	chatMember.JoinChannel(channel)
}

func (chatMember *ChatMember) PublishMessage(content string) {
	chatMember.msgChannel.PublishMessageAsync(Message{sender: chatMember.pseudo, content: content})
}

func (chatMember *ChatMember) ReceiveMessage(msg Message) {
	chatMember.msgChannelPort <- msg
}

func (chatMember *ChatMember) ReceiveNotification(notification string) {
	chatMember.msgChannelPort <- Message{content: notification, sender: ""}
}

func (chatMember *ChatMember) IsConnectedToAChannel() bool {
	return chatMember.msgChannel != nil
}

//Private methods

func (chatMember *ChatMember) quitServer(deliberatelyGone bool) {
	if chatMember.IsConnectedToAChannel() {
		if deliberatelyGone {
			chatMember.QuitChannel(LOGOUT)
		} else {
			chatMember.ReceiveNotification("Idle for a long time. I disconnect you")
			chatMember.QuitChannel(IDLE)
		}
	}
	chatMember.conn.Close() // TODO Attention
	chatMember.server.RemoveMember(chatMember)
}

func (chatMember *ChatMember) startListening(idlenessTimeout int) {

	go func() {
		reader := bufio.NewReader(chatMember.conn)
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				chatMember.quitPort <- struct{}{}
				return
			}
			chatMember.inputsPort <- strings.Trim(msg, " \n")
		}
	}()

	go func() {
		timeout := time.After(time.Duration(idlenessTimeout) * time.Second)
		for {
			select {
			case input := <-chatMember.inputsPort:
				{
					timeout = time.After(time.Duration(idlenessTimeout) * time.Second)
					chatMember.processInput(input)
				}
			case msg := <-chatMember.msgChannelPort:
				{
					fmt.Fprintf(chatMember.conn, msg.String())
				}
			case <-timeout:
				{
					chatMember.quitServer(false)
					return
				}
			case <-chatMember.quitPort:
				{
					chatMember.quitServer(true)
					return
				}
			}
		}
	}()
}

func (chatMember *ChatMember) processInput(input string) {

	if strings.HasPrefix(input, "/") {
		inputs := regexp.MustCompile("(\\s)+").Split(input, -1)
		cmd := strings.TrimPrefix(inputs[0], "/")
		var args []string
		if len(inputs) > 1 {
			args = inputs[1:]
		}
		_, resp := chatMember.server.cmdProcessor.processCommand(cmd, args, chatMember)
		chatMember.ReceiveMessage(Message{content: resp, sender: ""})

	} else {
		if chatMember.IsConnectedToAChannel() {
			chatMember.PublishMessage(input)
		} else {
			msg := "[INFO] You cannot write a message until you are connected to a channel \n"
			msg += "[INFO] Try /help command to see available commands :)"
			chatMember.ReceiveMessage(Message{content: msg, sender: ""})
		}
	}
}

func (chatMember *ChatMember) hasPseudo(pseudo string) bool {
	return strings.Compare(chatMember.pseudo, pseudo) == 0
}

/*********************************************************** class MessageChannel *****************************************************************/

type MessageChannel struct {
	members         []*ChatMember
	subscribePort   chan *ChatMember
	unSubscribePort chan UnSubscribingChatMember
	messagePort     chan Message
	name            string
	secret          string
}

func NewPrivateMessageChannel(name string) *MessageChannel {
	msgChannel := NewMessageChannel(name)
	msgChannel.secret = RandomString(10)
	return msgChannel
}

func NewMessageChannel(name string) *MessageChannel {
	msgChannel := &MessageChannel{
		members:         []*ChatMember{},
		subscribePort:   make(chan *ChatMember),
		unSubscribePort: make(chan UnSubscribingChatMember),
		messagePort:     make(chan Message),
		name:            name,
		secret:          "",
	}
	msgChannel.open()
	return msgChannel
}

func (msgChannel *MessageChannel) IsPrivate() bool {
	return strings.Compare(msgChannel.secret, "") != 0
}

func (msgChannel *MessageChannel) SubscribeAsync(chatMember *ChatMember) {
	go func() {
		msgChannel.subscribePort <- chatMember
	}()
}

func (msgChannel *MessageChannel) UnSubscribeAsync(chatMember *ChatMember, reason UnSubscribReason) {
	go func() {
		msgChannel.unSubscribePort <- UnSubscribingChatMember{chatMember: chatMember, reason: reason}
	}()
}

func (msgChannel *MessageChannel) PublishMessageAsync(message Message) {
	go func() {
		msgChannel.messagePort <- message
	}()
}

//Methods privées
func (msgChannel *MessageChannel) open() {
	go func() {
		for {
			select {
			case msg := <-msgChannel.messagePort:
				{
					msgChannel.broadcastMessage(msg)
				}
			case member := <-msgChannel.subscribePort:
				{
					msgChannel.addMember(member)
				}
			case member := <-msgChannel.unSubscribePort:
				{
					msgChannel.removeMember(member)
				}
			}
		}
	}()
}

func (msgChannel *MessageChannel) addMember(chatMember *ChatMember) {
	msgChannel.broadcastInfo(chatMember.pseudo + " joined the channel")
	msgChannel.members = append(msgChannel.members, chatMember)
	log.Println(chatMember.pseudo + " joined the channel [" + msgChannel.name + "]")
	chatMember.ReceiveNotification("Welcome on the channel [" + msgChannel.name + "]")
}

func (msgChannel *MessageChannel) removeMember(unSubscribingMember UnSubscribingChatMember) {
	member := unSubscribingMember.chatMember
	msgChannel.members = Filter(msgChannel.members, func(m *ChatMember) bool { return !m.hasPseudo(member.pseudo) })

	if unSubscribingMember.reason == IDLE {
		msgChannel.broadcastInfo(member.pseudo + " was idle too long and was disconnected")
		log.Println(member.pseudo + " seems to be out. Force disconnection.")
	} else if unSubscribingMember.reason == LOGOUT {
		msgChannel.broadcastInfo(member.pseudo + " has logged out.")
	} else if unSubscribingMember.reason == QUIT_CHANNEL {
		msgChannel.broadcastInfo(member.pseudo + " has changed channel.")
		member.ReceiveNotification("You have been retired from channel [" + msgChannel.name + "]")
	}
	log.Println(member.pseudo + "has been retired from channel [" + msgChannel.name + "]")

}

func (msgChannel *MessageChannel) broadcastMessage(msg Message) {
	for _, member := range msgChannel.members {
		member.ReceiveMessage(msg)
	}
}

func (msgChannel *MessageChannel) broadcastInfo(info string) {
	msgChannel.broadcastMessage(Message{sender: "", content: info})
}

//utils
func Filter(ss []*ChatMember, test func(*ChatMember) bool) (ret []*ChatMember) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

func RandomString(len int) string {
	bytes := make([]byte, len)
	for i := 0; i < len; i++ {
		bytes[i] = byte(65 + rand.Intn(25)) //A=65 and Z = 65+25
	}
	return string(bytes)
}

/********************************************************** class  Message ***********************************************************************/

type Message struct {
	sender  string
	content string
}

func (msg *Message) String() string {
	if len(msg.sender) > 0 {
		return msg.sender + ": " + msg.content + "\n"
	} else {
		return msg.content + "\n"
	}
}

/********************************************************** class  Message ***********************************************************************/

type UnSubscribReason int

const (
	IDLE UnSubscribReason = 1 + iota
	LOGOUT
	QUIT_CHANNEL
)

type UnSubscribingChatMember struct {
	chatMember *ChatMember
	reason     UnSubscribReason
}

/***************************************************** Commande Processor ***********************************************************************/

type CommandProcessor struct {
	commands []*Command
}

func newCommandProcessor() *CommandProcessor {
	processor := &CommandProcessor{
		commands: []*Command{},
	}
	return processor
}

func (processor *CommandProcessor) addCommand(command *Command) {
	processor.commands = append(processor.commands, command)
}

func (processor *CommandProcessor) processCommand(cmd string, args []string, member *ChatMember) (bool, string) {
	for _, v := range processor.commands {
		if 0 == strings.Compare((*v).name(), cmd) {
			return (*v).exec(args, member)
		}
	}
	return false, "unknown command"

}

type Command interface {
	exec([]string, *ChatMember) (bool, string)
	name() string
	description() string
}

/**********************************join_channel command ***************************/

type JoinChannel struct{}

func (joinChannel *JoinChannel) exec(args []string, member *ChatMember) (bool, string) {
	if len(args) < 1 {
		return false, "incorrect number of arguments."
	}
	channelName := args[0]
	channel := member.server.FindChannel(channelName)
	if channel == nil {
		return false, "Channel [" + channelName + "] does'nt exist."
	}

	if member.IsConnectedToAChannel() && strings.Compare(channel.name, member.msgChannel.name) == 0 {
		return false, "You have already joined the channel [" + channel.name + "]"
	}

	if channel.IsPrivate() {
		if len(args) < 2 {
			return false, "This channel is private. Please submit the additional secret to join it."
		}
		secret := args[1]
		if strings.Compare(secret, channel.secret) != 0 {
			return false, "The submitted secret is incorrect."
		}
	}
	member.ChangeChannel(channel)
	return true, "Joining channel " + channelName + "..."
}

func (joinChannel *JoinChannel) name() string {
	return "join"
}

func (joinChannel *JoinChannel) description() string {
	return "Join a channel : /join 'channel_name'"
}

func newJoinChannel() *JoinChannel {
	return &JoinChannel{}
}

/********************************** quit_channel command ***************************/

type QuitChannel struct{}

func (quiteChannel *QuitChannel) exec(args []string, member *ChatMember) (bool, string) {

	if !member.IsConnectedToAChannel() {
		return false, "You are not connected to any channel."
	}
	msg := "Quiting channel  " + member.msgChannel.name + "..."
	member.QuitChannel(QUIT_CHANNEL)
	return true, msg
}

func (quiteChannel *QuitChannel) name() string {
	return "quit"
}

func (quiteChannel *QuitChannel) description() string {
	return "Quit a channel : /quit 'channel_name'"
}

func newQuitChannel() *QuitChannel {
	return &QuitChannel{}
}

/********************************** help command ***************************/

type HelpCMD struct{}

func (helpCMD *HelpCMD) exec(args []string, member *ChatMember) (bool, string) {
	res := ""
	for _, cmd := range member.server.cmdProcessor.commands {
		res = res + (*cmd).name() + " -> " + (*cmd).description() + "\n"
	}
	res = strings.TrimSuffix(res, "\n")
	return true, res
}

func (helpCMD *HelpCMD) name() string {
	return "help"
}

func (helpCMD *HelpCMD) description() string {
	return "Display available commands : /help"
}

func newHelpCMD() *HelpCMD {
	return &HelpCMD{}
}

/**********************************create_channel command ***************************/

type CreateChannel struct{}

func (createChannel *CreateChannel) exec(args []string, member *ChatMember) (bool, string) {
	if len(args) < 1 {
		return false, "Please give the name of the channel."
	}
	channelName := args[0]
	channel := member.server.FindChannel(channelName)
	if channel != nil {
		return false, "This channel name is already used."
	}

	if len(args) > 1 {
		option := args[1]
		if strings.Compare(option, "--private") == 0 {
			channel = NewPrivateMessageChannel(channelName)
		} else {
			return false, "Only the --private option is accepted in addition to create a private channel."
		}
	} else {
		channel = NewMessageChannel(channelName)
	}
	member.server.AddChannel(channel)
	var res string
	if channel.IsPrivate() {
		res = "Channel [" + channel.name + "] has been created with secret [" + channel.secret + "]."
	} else {
		res = "Channel [" + channel.name + "] has been created."
	}
	return true, res

}

func (createChannel *CreateChannel) name() string {
	return "create"
}

func (createChannel *CreateChannel) description() string {
	return "Create a channel : /create 'channel_name'"
}

func newCreateChannel() *CreateChannel {
	return &CreateChannel{}
}

/***************************************************** Server ***********************************************************************/

type Server struct {
	channels     []*MessageChannel
	chatMembers  []*ChatMember
	cmdProcessor *CommandProcessor
}

func newServer() *Server {
	return &Server{
		channels:     []*MessageChannel{},
		chatMembers:  []*ChatMember{},
		cmdProcessor: newCommandProcessor(),
	}
}

func (server *Server) AddMember(member *ChatMember) {
	server.chatMembers = append(server.chatMembers, member)
	member.ReceiveNotification("Welcome on board " + member.pseudo)
	log.Println(member.pseudo + " Logged in.")
}

func (server *Server) RemoveMember(member *ChatMember) {
	server.chatMembers = Filter(server.chatMembers, func(member *ChatMember) bool { return !member.hasPseudo(member.pseudo) })
	log.Println(member.pseudo + " Logged out.")
}

func (server *Server) AddChannel(channel *MessageChannel) {
	if channel.IsPrivate() {
		log.Println("New private channel " + channel.name + " created with secret [" + channel.secret + "]")
	} else {
		log.Println("New public channel " + channel.name + " created")
	}
	server.channels = append(server.channels, channel)
}

func (server *Server) FindChannel(name string) *MessageChannel {
	for _, channel := range server.channels {
		if strings.Compare(channel.name, name) == 0 {
			return channel
		}
	}
	return nil
}

func (server *Server) AddCommand(command Command) {
	server.cmdProcessor.addCommand(&command)
}

/***************************************************** Main ***********************************************************************/

func handleNewConnection(conn net.Conn, server *Server) {
	fmt.Fprintf(conn, "Nickname? ")
	reader := bufio.NewReader(conn)
	pseudo, err := reader.ReadString('\n')
	if err != nil {
		log.Println(err)
		return
	}
	member := NewChatMember(strings.Trim(pseudo, " \n"), conn, server, 600)
	member.JoinChannel(server.FindChannel("general"))
}

func main() {
	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Chat server is up")

	server := newServer()
	server.AddCommand(newJoinChannel())
	server.AddCommand(newQuitChannel())
	server.AddCommand(newHelpCMD())
	server.AddCommand(newCreateChannel())

	general := NewMessageChannel("general")
	random := NewMessageChannel("random")
	server.AddChannel(random)
	server.AddChannel(general)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go handleNewConnection(conn, server)
	}
}
