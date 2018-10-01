package main

import (
    "net"
    "bufio"
    "log"
    "fmt"
    "strings"
)


/*************************** Class ChatMember **************************************/

type ChatMember struct {
  msgChannel *MessageChannel
  pseudo string
  msgChannelPort chan Message
  connexionPort chan string
  conn net.Conn
}

func newChatMember(pseudo string, conn net.Conn) * ChatMember {
  chatMember := &ChatMember{
    pseudo: pseudo,
    msgChannelPort: make(chan Message),
    connexionPort: make(chan string),
    conn:conn,
  }
  fmt.Fprintf(chatMember.conn, "Welcome on board %s\n", chatMember.pseudo);
  return chatMember;
}

func (chatMember *ChatMember) joinChannel(msgChannel *MessageChannel)  {
  chatMember.msgChannel = msgChannel
  msgChannel.addMember(chatMember)
}

func (chatMember *ChatMember) PublishMessage(content string )  {
  msg := Message{sender: chatMember.pseudo, content:content}
  chatMember.msgChannel.pushMessage(msg)
}

func (chatMember *ChatMember) receiveMessage(msg Message)  {
    chatMember.msgChannelPort <- msg
}

func (chatMember *ChatMember) startListening()  {

    go func ()  {
      reader := bufio.NewReader(chatMember.conn)
      for {
          msg,err := reader.ReadString('\n')
          if err != nil {
              log.Println(err)
              return
          }
          chatMember.connexionPort <- msg
      }
    }()

    go func ()  {
      for {
        select {
          case content := <- chatMember.connexionPort : {
            chatMember.PublishMessage(content)
           }
           case msg := <- chatMember.msgChannelPort : {
             if msg.concerns(chatMember) {
                 fmt.Fprintf(chatMember.conn, msg.String());
               }
           }
        }
      }
    }()
}



/***************************** Class  MessageChannel ********************************/

type MessageChannel struct {
  members []*ChatMember
}


func (msgChannel *MessageChannel) addMember(chatMember *ChatMember)  {
    msgChannel.members = append(msgChannel.members, chatMember)
    msgChannel.pushMessage( Message{ sender: "", content: chatMember.pseudo + " joined the channel" , ignore: chatMember.pseudo} )
}

func newMessageChannel() * MessageChannel {
  return &MessageChannel {
    members: []*ChatMember{},
  }
}

func (msgChannel *MessageChannel) pushMessage(msg Message)  {
  go func ()  {
    for _,member := range(msgChannel.members){
      member.receiveMessage(msg)
    }
  }();
}



/***************************** Class  Message ********************************/

type Message struct {
  sender string
  content string
  ignore string
}

func (msg * Message) String() string {
    if len(msg.sender) > 0{
        return  msg.sender + ": "+msg.content+"\n"
    }else{
        return msg.content+"\n"
    }
}

func (msg * Message) concerns(chatMember * ChatMember) bool {
  return len(msg.sender) == 0 && strings.Compare(msg.ignore, chatMember.pseudo) !=0 || len(msg.sender) > 0 && strings.Compare(msg.sender, chatMember.pseudo) !=0
}


// +slide
func handleConnection(conn net.Conn, msgChannel *MessageChannel) {
    fmt.Fprintf(conn, "Nickname? ");
    reader := bufio.NewReader(conn)
    pseudo,err := reader.ReadString('\n')
    if err != nil {
        log.Println(err)
        return
    }
    chatMember := newChatMember(strings.Trim(pseudo," \n"),conn)
    chatMember.joinChannel(msgChannel)
    chatMember.startListening()
}

func main() {
    listener, err := net.Listen("tcp", "localhost:1234")
    if err != nil {
        log.Fatal(err)
    }

    mainMsgChannel := newMessageChannel()


    for {
        conn, err := listener.Accept()
        if err != nil {
            log.Println(err)
            continue
        }
        go handleConnection(conn,mainMsgChannel)
    }
}
// ---
// Local Variables:
// compile-command: "go run *.go"
// End:
