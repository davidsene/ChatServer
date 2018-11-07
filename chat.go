package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)


/*********************************************************** class ChatMember *****************************************************************/

type ChatMember struct {
	msgChannel     *MessageChannel
	pseudo         string
	msgChannelPort chan Message
	connexionPort  chan string
	quitPort       chan struct{}
	conn           net.Conn
}

func NewChatMember(pseudo string, conn net.Conn) *ChatMember {
	chatMember := &ChatMember{
		pseudo:         pseudo,
		msgChannelPort: make(chan Message, 1),
		connexionPort:  make(chan string),
		quitPort:       make(chan struct{}),
		conn:           conn,
	}
	fmt.Fprintf(chatMember.conn, "Welcome on board %s\n", chatMember.pseudo)
	return chatMember
}

func (chatMember *ChatMember) JoinChannel(msgChannel *MessageChannel) {
	chatMember.msgChannel = msgChannel
	msgChannel.Subscribe(chatMember)
}

func (chatMember *ChatMember) QuitChannel(deliberatelyGone bool) {
	if !deliberatelyGone {
		fmt.Fprintf(chatMember.conn, "Idle for a long time. I disconnect you")
	}
	chatMember.conn.Close()
	chatMember.msgChannel.UnSubscribe(chatMember, deliberatelyGone)
}

func (chatMember *ChatMember) PublishMessage(content string) {
	chatMember.msgChannel.PublishMessage(Message{sender: chatMember.pseudo, content: content})
}

func (chatMember *ChatMember) ReceiveMessage(msg Message) {
	chatMember.msgChannelPort <- msg
}

func (chatMember *ChatMember) StartListeningMessages(idlenessTimeout int) {

	go func() {
		reader := bufio.NewReader(chatMember.conn)
		for {
			msg, err := reader.ReadString('\n')
			if err != nil {
				chatMember.quitPort <- struct{}{}
				return
			}
			chatMember.connexionPort <- strings.Trim(msg," \n")
		}
	}()

	go func() {
		timeout := time.After(time.Duration(idlenessTimeout) * time.Second)
		for {
			select {
			case content := <-chatMember.connexionPort:
				{
					timeout = time.After(time.Duration(idlenessTimeout) * time.Second)
					chatMember.PublishMessage(content)
				}
			case msg := <-chatMember.msgChannelPort:
				{
					if msg.Concerns(chatMember) {
						fmt.Fprintf(chatMember.conn, msg.String())
					}
				}
			case <-timeout:
				{
					chatMember.QuitChannel(false)
					return
				}
			case <-chatMember.quitPort:
				{
					chatMember.QuitChannel(true)
					return
				}
			}
		}
	}()

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
}

func NewMessageChannel() *MessageChannel {
	msgChannel := &MessageChannel{
		members:         []*ChatMember{},
		subscribePort:   make(chan *ChatMember),
		unSubscribePort: make(chan UnSubscribingChatMember),
		messagePort:     make(chan Message),
	}
	return msgChannel
}

func (msgChannel *MessageChannel) Subscribe(chatMember *ChatMember) {
	go func() {
		msgChannel.subscribePort <- chatMember
	}()
}

func (msgChannel *MessageChannel) UnSubscribe(chatMember *ChatMember, deliberatelyGone bool) {
	go func() {
		chatMember.msgChannel.unSubscribePort <- UnSubscribingChatMember{chatMember: chatMember, deliberatelyGone: deliberatelyGone}
	}()
}

func (msgChannel *MessageChannel) PublishMessage(message Message) {
	go func() {
		msgChannel.messagePort <- message
	}()
}

func (msgChannel *MessageChannel) Open() {
	go func() {
		for {
			select {
			case msg := <-msgChannel.messagePort:
				{
					msgChannel.broadcastMessage(msg)
				}
			case member:= <-msgChannel.subscribePort:
				{
					msgChannel.addMember(member)
				}
			case member:= <-msgChannel.unSubscribePort:
				{
					msgChannel.removeMember(member)
				}
			}
		}
	}()
}

//Method privées

func (msgChannel *MessageChannel) addMember(chatMember *ChatMember) {
	msgChannel.members = append(msgChannel.members, chatMember)
	msgChannel.broadcastMessage(Message{sender: "", content: chatMember.pseudo + " joined the channel", ignore: chatMember.pseudo})
	log.Println("Login of " + chatMember.pseudo)
}

func (msgChannel *MessageChannel) removeMember(unSubscribingMember UnSubscribingChatMember) {
	deliberatelyGone := unSubscribingMember.deliberatelyGone
	chatMember := unSubscribingMember.chatMember
	if !deliberatelyGone {
		msgChannel.broadcastMessage(Message{sender: "", content: chatMember.pseudo + " was idle too long and was disconnected", ignore: chatMember.pseudo})
		log.Println(chatMember.pseudo + " seems to be out. Force disconnection.")
	} else {
		msgChannel.broadcastMessage(Message{sender: "", content: chatMember.pseudo + " is gone", ignore: chatMember.pseudo})
		log.Println(chatMember.pseudo + " logged out.")
	}
	msgChannel.members = Filter(msgChannel.members, func(member *ChatMember) bool { return ! member.hasPseudo(chatMember.pseudo) })
}

func Filter(ss []*ChatMember, test func(*ChatMember) bool) (ret []*ChatMember) {
	for _, s := range ss {
		if test(s) {
			ret = append(ret, s)
		}
	}
	return
}

func (msgChannel *MessageChannel) broadcastMessage(msg Message) {
	go func() {
		for _, member := range msgChannel.members {
			member.ReceiveMessage(msg)
		}
	}()
}


/********************************************************** class  Message ***********************************************************************/

type Message struct {
	sender  string
	content string
	ignore  string
}

func (msg *Message) String() string {
	if len(msg.sender) > 0 {
		return msg.sender + ": " + msg.content + "\n"
	} else {
		return msg.content + "\n"
	}
}

func (msg *Message) Concerns(chatMember *ChatMember) bool {
	return len(msg.sender) == 0 && !chatMember.hasPseudo(msg.ignore) || len(msg.sender) > 0 && !chatMember.hasPseudo(msg.sender)
}


/********************************************************** class  Message ***********************************************************************/

type UnSubscribingChatMember struct {
	chatMember       *ChatMember
	deliberatelyGone bool
}



func handleNewConnection(conn net.Conn, msgChannel *MessageChannel) {
	fmt.Fprintf(conn, "Nickname? ")
	reader := bufio.NewReader(conn)
	pseudo, err := reader.ReadString('\n')
	if err != nil {
		log.Println(err)
		return
	}
	chatMember := NewChatMember(strings.Trim(pseudo, " \n"), conn)
	chatMember.JoinChannel(msgChannel)
	chatMember.StartListeningMessages(30)
}


func main() {
	listener, err := net.Listen("tcp", ":1234")
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Chat server is up")
	mainMsgChannel := NewMessageChannel()
	mainMsgChannel.Open()

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go handleNewConnection(conn, mainMsgChannel)
	}
}